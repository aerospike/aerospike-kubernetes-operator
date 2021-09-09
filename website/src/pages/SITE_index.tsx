import { Link } from '@docusaurus/router';
import useDocusaurusContext from '@docusaurus/useDocusaurusContext';
import SkyhookHeroImage from '@site/static/img/hero.jpg';
import Layout from '@theme/Layout';
import clsx from 'clsx';
import React from 'react';
import styles from './index.module.css';

function HomepageHeader() {
  const { siteConfig } = useDocusaurusContext();
  return (
    <header className={clsx('hero hero--dark', styles.heroBanner)}>
      <div className="container">
        <h1 className="hero__title">{siteConfig.title}</h1>

        <Link to="docs/intro" className={styles.heroLink}>
          <div className="hero__subtitle">{siteConfig.tagline}</div>

          <img src={SkyhookHeroImage} width="100%"/>
        </Link>

      </div>
    </header>
  );
}

export default function Home(): JSX.Element {
  const { siteConfig } = useDocusaurusContext();
  return (
    // <main className={styles.contentBody}>
    <Layout
      wrapperClassName={styles.contentBody}
      title={`${siteConfig.title}`}
      description="Use Skyhook to quickly get your existing Redis applications running on Aerospike. Skyhook is a Redis API-compatible client for the Aerospike Database."
    >
      <HomepageHeader />
    </Layout>
    // </main>
  );
}
